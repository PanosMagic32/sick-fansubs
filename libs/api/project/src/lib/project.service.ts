import { BadRequestException, Injectable, NotFoundException } from '@nestjs/common';
import { InjectModel } from '@nestjs/mongoose';
import { Model, Types } from 'mongoose';

import { MediaService } from '@api/media';

import { CreateProjectDto } from './dto/create-project.dto';
import { UpdateProjectDto } from './dto/update-project.dto';
import { Project, ProjectDocument } from './schemas/project.schema';

const RESOLUTION_ERROR =
  'Each batch download link must have at least one complete resolution pair (1080p or 2160p) with both torrent and magnet links';

@Injectable()
export class ProjectService {
  constructor(
    @InjectModel(Project.name) private readonly projectModel: Model<ProjectDocument>,
    private readonly mediaService: MediaService,
  ) {}

  private sanitizeEditedMetadata<T extends { updatedBy?: unknown; updatedAt?: Date | string }>(entity: T): T {
    if (!entity.updatedBy) {
      entity.updatedAt = undefined;
    }

    return entity;
  }

  private slugify(value: string): string {
    return value
      .normalize('NFKD')
      .replace(/[\u0300-\u036f]/g, '')
      .toLowerCase()
      .trim()
      .replace(/[^a-z0-9\s-]/g, '')
      .replace(/\s+/g, '-')
      .replace(/-+/g, '-');
  }

  private async createUniqueSlug(title: string, excludedId?: string): Promise<string> {
    const baseSlug = this.slugify(title) || 'project';
    let slug = baseSlug;
    let counter = 2;
    let existing = await this.projectModel
      .findOne({
        slug,
        ...(excludedId ? { _id: { $ne: excludedId } } : {}),
      })
      .select('_id')
      .lean();

    while (existing) {
      slug = `${baseSlug}-${counter}`;
      counter += 1;

      existing = await this.projectModel
        .findOne({
          slug,
          ...(excludedId ? { _id: { $ne: excludedId } } : {}),
        })
        .select('_id')
        .lean();
    }

    return slug;
  }

  private validateBatchLinks(
    links?: {
      downloadLink?: string;
      downloadLinkTorrent?: string;
      downloadLink4k?: string;
      downloadLink4kTorrent?: string;
    }[],
  ): void {
    if (!links || links.length === 0) return;

    for (const link of links) {
      const has1080p = !!(link.downloadLink?.trim() && link.downloadLinkTorrent?.trim());
      const has2160p = !!(link.downloadLink4k?.trim() && link.downloadLink4kTorrent?.trim());

      if (!has1080p && !has2160p) {
        throw new BadRequestException(RESOLUTION_ERROR);
      }
    }
  }

  async create(createProjectDto: CreateProjectDto, creatorId: string): Promise<Project> {
    this.validateBatchLinks(createProjectDto.batchDownloadLinks);

    const slug = await this.createUniqueSlug(createProjectDto.title);
    const createProject = await this.projectModel.create({
      ...createProjectDto,
      creator: new Types.ObjectId(creatorId),
      slug,
    });
    return this.sanitizeEditedMetadata(createProject);
  }

  async findAll(
    pageSize: number,
    currentPage: number,
    // startId?: string
  ): Promise<{ projects: Project[]; count: number }> {
    const query = this.projectModel
      // The below commented-out object in find is a possible way to improve performance in database search
      // .find({
      //   _id: {
      //     $gt: startId,
      //   },
      // })
      .find()
      .populate('creator', 'username avatar')
      .populate('updatedBy', 'username avatar')
      .sort({ dateTimeCreated: 'desc' });

    query.skip(pageSize * currentPage).limit(pageSize);

    const projects = await query.exec();
    const count = await this.projectModel.countDocuments();

    return { projects: projects.map((project) => this.sanitizeEditedMetadata(project)), count };
  }

  async findOne(id: string): Promise<Project | undefined> {
    const project = await this.projectModel
      .findOne({ _id: id })
      .populate('creator', 'username avatar')
      .populate('updatedBy', 'username avatar');
    if (project) return this.sanitizeEditedMetadata(project);
    throw new NotFoundException();
  }

  async update(id: string, updateProjectDto: UpdateProjectDto, actorId: string): Promise<Project | undefined | null> {
    const existingProject = await this.projectModel.findById(id).select('thumbnail').exec();
    if (existingProject) {
      this.validateBatchLinks(updateProjectDto.batchDownloadLinks);

      const payload = { ...updateProjectDto, updatedBy: new Types.ObjectId(actorId) } as UpdateProjectDto & {
        slug?: string;
        updatedBy: Types.ObjectId;
      };

      if (updateProjectDto.title) {
        payload.slug = await this.createUniqueSlug(updateProjectDto.title, id);
      }

      const updatedProject = await this.projectModel
        .findByIdAndUpdate(id, payload, { new: true, runValidators: true })
        .exec();

      if (updatedProject && existingProject.thumbnail !== updatedProject.thumbnail) {
        await this.mediaService.deleteManagedImageByUrl(existingProject.thumbnail);
      }

      return updatedProject;
    }

    throw new NotFoundException();
  }

  async remove(id: string) {
    const deletedProject = await this.projectModel.findByIdAndDelete(id).exec();
    if (!deletedProject) {
      throw new NotFoundException();
    }

    await this.mediaService.deleteManagedImageByUrl(deletedProject.thumbnail);

    return deletedProject;
  }
}
