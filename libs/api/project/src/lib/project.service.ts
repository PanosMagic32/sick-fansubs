import { Injectable, NotFoundException } from '@nestjs/common';
import { InjectModel } from '@nestjs/mongoose';
import { Model } from 'mongoose';

import { CreateProjectDto } from './dto/create-project.dto';
import { UpdateProjectDto } from './dto/update-project.dto';
import { Project, ProjectDocument } from './schemas/project.schema';

@Injectable()
export class ProjectService {
  constructor(@InjectModel(Project.name) private readonly projectModel: Model<ProjectDocument>) {}

  async create(createProjectDto: CreateProjectDto): Promise<Project> {
    const createProject = await this.projectModel.create(createProjectDto);
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
      .sort({ dateTimeCreated: 'desc' });

    if (pageSize && currentPage) {
      query.skip(pageSize * currentPage).limit(pageSize);
    }

    const projects = await query.exec();
    const count = await this.projectModel.countDocuments();

    return { projects, count };
  }

  async findOne(id: string): Promise<Project | undefined> {
    const project = await this.projectModel.findOne({ _id: id });
    if (project) return project;
    throw new NotFoundException();
  }

  async update(id: string, updateProjectDto: UpdateProjectDto): Promise<Project | undefined | null> {
    const project = await this.findOne(id);
    if (project) return this.projectModel.findByIdAndUpdate({ _id: id }, updateProjectDto).exec();
    throw new NotFoundException();
  }

  async remove(id: string) {
    const deletedProject = await this.projectModel.findByIdAndDelete({ _id: id }).exec();
    return deletedProject;
  }
}
