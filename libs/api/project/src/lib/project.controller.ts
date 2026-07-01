import {
  UnauthorizedException,
  Body,
  Controller,
  Delete,
  Get,
  Param,
  Patch,
  Post,
  Query,
  Req,
  UseGuards,
} from '@nestjs/common';
import { ApiParam, ApiTags } from '@nestjs/swagger';
import { Throttle } from '@nestjs/throttler';

import { PaginationDto, ParseMongoIdPipe } from '@api/shared';
import { AdminGuard, JwtAuthGuard } from '@api/user';

import { CreateProjectDto } from './dto/create-project.dto';
import { UpdateProjectDto } from './dto/update-project.dto';
import { ProjectService } from './project.service';
import { Project } from './schemas/project.schema';

@ApiTags('Project')
@Controller('project')
export class ProjectController {
  constructor(private readonly projectService: ProjectService) {}

  private getActorIdFromRequest(req: { user?: { sub?: string } }): string {
    if (!req.user?.sub) {
      throw new UnauthorizedException('User not authenticated.');
    }
    return req.user.sub;
  }

  @Post()
  @UseGuards(JwtAuthGuard, AdminGuard)
  @Throttle({ default: { limit: 30, ttl: 60000 } })
  async create(@Body() createProjectDto: CreateProjectDto, @Req() req: { user?: { sub?: string } }) {
    return this.projectService.create(createProjectDto, this.getActorIdFromRequest(req));
  }

  @Get()
  async findAll(@Query() pagination: PaginationDto): Promise<{ projects: Project[]; count: number }> {
    return this.projectService.findAll(pagination.pagesize, pagination.page);
  }

  @ApiParam({ name: 'id' })
  @Get(':id')
  async findOne(@Param('id', ParseMongoIdPipe) id: string) {
    return this.projectService.findOne(id);
  }

  @ApiParam({ name: 'id' })
  @Patch(':id')
  @UseGuards(JwtAuthGuard, AdminGuard)
  @Throttle({ default: { limit: 20, ttl: 60000 } })
  async update(
    @Param('id', ParseMongoIdPipe) id: string,
    @Body() updateProjectDto: UpdateProjectDto,
    @Req() req: { user?: { sub?: string } },
  ) {
    return this.projectService.update(id, updateProjectDto, this.getActorIdFromRequest(req));
  }

  @ApiParam({ name: 'id' })
  @Delete(':id')
  @UseGuards(JwtAuthGuard, AdminGuard)
  @Throttle({ default: { limit: 20, ttl: 60000 } })
  async remove(@Param('id', ParseMongoIdPipe) id: string) {
    return this.projectService.remove(id);
  }
}
