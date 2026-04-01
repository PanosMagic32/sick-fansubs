import { Injectable, NotFoundException } from '@nestjs/common';
import { InjectModel } from '@nestjs/mongoose';
import { Model, Types } from 'mongoose';

import { CreateProjectDto } from './dto/create-project.dto';
import { UpdateProjectDto } from './dto/update-project.dto';
import { Project, ProjectDocument } from './schemas/project.schema';

@Injectable()
export class ProjectService {
  constructor(@InjectModel(Project.name) private readonly projectModel: Model<ProjectDocument>) {}

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

  async create(createProjectDto: CreateProjectDto, creatorId: string): Promise<Project> {
    const slug = await this.createUniqueSlug(createProjectDto.title);
    const createProject = await this.projectModel.create({
      ...createProjectDto,
      creator: new Types.ObjectId(creatorId),
      slug,
    });
    return createProject;
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

    if (pageSize && currentPage) {
      query.skip(pageSize * currentPage).limit(pageSize);
    }

    const projects = await query.exec();
    const count = await this.projectModel.countDocuments();

    return { projects, count };
  }

  async findOne(id: string): Promise<Project | undefined> {
    const project = await this.projectModel
      .findOne({ _id: id })
      .populate('creator', 'username avatar')
      .populate('updatedBy', 'username avatar');
    if (project) return project;
    throw new NotFoundException();
  }

  async update(id: string, updateProjectDto: UpdateProjectDto, actorId: string): Promise<Project | undefined | null> {
    const project = await this.findOne(id);
    if (project) {
      const payload = { ...updateProjectDto, updatedBy: new Types.ObjectId(actorId) } as UpdateProjectDto & {
        slug?: string;
        updatedBy: Types.ObjectId;
      };

      if (updateProjectDto.title) {
        payload.slug = await this.createUniqueSlug(updateProjectDto.title, id);
      }

      return this.projectModel.findByIdAndUpdate({ _id: id }, payload).exec();
    }
    throw new NotFoundException();
  }

  async remove(id: string) {
    const deletedProject = await this.projectModel.findByIdAndDelete({ _id: id }).exec();
    return deletedProject;
  }
}
